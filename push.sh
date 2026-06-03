TAG=0.2.8
FE_IMG=aakkiiff/chowkidar:frontend-$TAG
# BE_IMG=aakkiiff/chowkidar:server-$TAG
# AGNT_IMG=aakkiiff/chowkidar:agent-$TAG

# Build the images
# docker build -t $BE_IMG ./server --no-cache
docker build -t $FE_IMG ./frontend --no-cache
# docker build -t $AGNT_IMG ./agent --no-cache

# Push the images to Docker Hub
# docker push $BE_IMG
docker push $FE_IMG
# docker push $AGNT_IMG

